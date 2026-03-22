/**
 * Virtual List Module
 * Provides virtual scrolling for efficient rendering of large lists
 */

const VirtualList = (function() {
    // 默认配置
    const DEFAULT_CONFIG = {
        itemHeight: 24,          // 每个项目的估计高度（像素）
        containerHeight: 300,    // 容器高度（像素）
        overscan: 5,             // 预渲染的额外项目数
        threshold: 100,          // 启用虚拟滚动的阈值（项目数）
    };

    /**
     * 创建虚拟列表实例
     * @param {Object} options - 配置选项
     * @param {HTMLElement} options.container - 容器元素
     * @param {number} [options.itemHeight] - 每个项目的估计高度
     * @param {number} [options.containerHeight] - 容器高度
     * @param {number} [options.overscan] - 预渲染的额外项目数
     * @param {number} [options.threshold] - 启用虚拟滚动的阈值
     * @param {Function} options.renderItem - 渲染单个项目的函数
     * @returns {Object} 虚拟列表实例
     */
    function create(options) {
        const config = { ...DEFAULT_CONFIG, ...options };
        const {
            container,
            itemHeight,
            containerHeight,
            overscan,
            threshold,
            renderItem
        } = config;

        if (!container) {
            throw new Error('Container element is required');
        }

        // 内部状态
        let data = [];
        let scrollTop = 0;
        let isVirtualEnabled = false;
        let scrollListener = null;

        // 创建内部结构
        const wrapper = document.createElement('div');
        wrapper.style.cssText = 'position: relative; overflow: hidden;';

        const content = document.createElement('div');
        content.style.cssText = 'position: relative;';

        // 应用容器样式
        container.style.overflowY = 'auto';
        container.style.height = `${containerHeight}px`;

        wrapper.appendChild(content);
        container.appendChild(wrapper);

        /**
         * 计算可见范围
         * @returns {Object} { startIndex, endIndex, offsetY }
         */
        function calculateVisibleRange() {
            const visibleCount = Math.ceil(containerHeight / itemHeight);
            const startIndex = Math.max(0, Math.floor(scrollTop / itemHeight) - overscan);
            const endIndex = Math.min(data.length, startIndex + visibleCount + overscan * 2);
            const offsetY = startIndex * itemHeight;

            return { startIndex, endIndex, offsetY };
        }

        /**
         * 渲染可见项目
         */
        function renderVisibleItems() {
            if (!isVirtualEnabled) {
                // 简单模式：直接渲染所有项目
                content.innerHTML = '';
                content.style.height = 'auto';
                
                if (data.length === 0) {
                    if (config.renderEmpty) {
                        const emptyElement = config.renderEmpty();
                        content.appendChild(emptyElement);
                    }
                    return;
                }

                data.forEach((item, index) => {
                    const element = renderItem(item, index);
                    content.appendChild(element);
                });
                return;
            }

            // 虚拟滚动模式
            const { startIndex, endIndex, offsetY } = calculateVisibleRange();

            // 设置内容高度以支持滚动
            content.style.height = `${data.length * itemHeight}px`;

            // 清空并重新渲染
            content.innerHTML = '';

            // 创建定位容器
            const itemsContainer = document.createElement('div');
            itemsContainer.style.cssText = `position: absolute; top: ${offsetY}px; left: 0; right: 0;`;

            // 渲染可见项目
            for (let i = startIndex; i < endIndex; i++) {
                const element = renderItem(data[i], i);
                element.style.height = `${itemHeight}px`;
                element.style.boxSizing = 'border-box';
                itemsContainer.appendChild(element);
            }

            content.appendChild(itemsContainer);
        }

        /**
         * 处理滚动事件
         */
        function handleScroll() {
            const newScrollTop = container.scrollTop;
            if (Math.abs(newScrollTop - scrollTop) >= itemHeight / 2) {
                scrollTop = newScrollTop;
                requestAnimationFrame(renderVisibleItems);
            }
        }

        /**
         * 设置数据
         * @param {Array} newData - 新数据数组
         */
        function setData(newData) {
            data = newData || [];

            // 根据数据量决定是否启用虚拟滚动
            const shouldEnableVirtual = data.length > threshold;

            if (shouldEnableVirtual !== isVirtualEnabled) {
                isVirtualEnabled = shouldEnableVirtual;

                if (shouldEnableVirtual && !scrollListener) {
                    // 启用虚拟滚动，添加滚动监听
                    scrollListener = handleScroll;
                    container.addEventListener('scroll', scrollListener, { passive: true });
                } else if (!shouldEnableVirtual && scrollListener) {
                    // 禁用虚拟滚动，移除滚动监听
                    container.removeEventListener('scroll', scrollListener);
                    scrollListener = null;
                }
            }

            scrollTop = container.scrollTop;
            renderVisibleItems();
        }

        /**
         * 滚动到指定索引
         * @param {number} index - 目标索引
         */
        function scrollToIndex(index) {
            if (index < 0 || index >= data.length) return;

            const targetScrollTop = index * itemHeight;
            container.scrollTop = targetScrollTop;
        }

        /**
         * 更新配置
         * @param {Object} newConfig - 新配置
         */
        function updateConfig(newConfig) {
            Object.assign(config, newConfig);
            
            if (newConfig.containerHeight) {
                container.style.height = `${newConfig.containerHeight}px`;
            }
            
            renderVisibleItems();
        }

        /**
         * 销毁实例
         */
        function destroy() {
            if (scrollListener) {
                container.removeEventListener('scroll', scrollListener);
            }
            container.innerHTML = '';
        }

        /**
         * 获取当前状态
         * @returns {Object} 当前状态
         */
        function getState() {
            return {
                dataLength: data.length,
                isVirtualEnabled,
                scrollTop,
                containerHeight,
                itemHeight
            };
        }

        return {
            setData,
            scrollToIndex,
            updateConfig,
            destroy,
            getState,
            render: renderVisibleItems
        };
    }

    /**
     * 创建分页列表实例
     * @param {Object} options - 配置选项
     * @param {HTMLElement} options.container - 容器元素
     * @param {number} [options.pageSize] - 每页显示数量
     * @param {Function} options.renderItem - 渲染单个项目的函数
     * @param {Function} [options.renderEmpty] - 渲染空状态的函数
     * @returns {Object} 分页列表实例
     */
    function createPaginated(options) {
        const {
            container,
            pageSize = 50,
            renderItem,
            renderEmpty
        } = options;

        if (!container) {
            throw new Error('Container element is required');
        }

        // 内部状态
        let data = [];
        let currentPage = 1;
        let totalPages = 1;

        // 创建结构
        const listContainer = document.createElement('div');
        listContainer.className = 'paginated-list-content';

        const paginationContainer = document.createElement('div');
        paginationContainer.className = 'pagination-controls';
        paginationContainer.style.cssText = 'display: flex; justify-content: center; gap: 8px; margin-top: 16px; flex-wrap: wrap;';

        container.appendChild(listContainer);
        container.appendChild(paginationContainer);

        /**
         * 渲染当前页
         */
        function renderCurrentPage() {
            listContainer.innerHTML = '';

            const startIndex = (currentPage - 1) * pageSize;
            const endIndex = Math.min(startIndex + pageSize, data.length);
            const pageData = data.slice(startIndex, endIndex);

            if (pageData.length === 0) {
                if (renderEmpty) {
                    const emptyElement = renderEmpty();
                    listContainer.appendChild(emptyElement);
                }
                return;
            }

            pageData.forEach((item, index) => {
                const element = renderItem(item, startIndex + index);
                listContainer.appendChild(element);
            });
        }

        /**
         * 渲染分页控件
         */
        function renderPagination() {
            paginationContainer.innerHTML = '';

            if (totalPages <= 1) return;

            // 上一页按钮
            const prevBtn = createPaginationButton('‹', currentPage > 1, () => {
                if (currentPage > 1) {
                    currentPage--;
                    renderCurrentPage();
                    renderPagination();
                }
            });
            paginationContainer.appendChild(prevBtn);

            // 页码按钮
            const maxVisiblePages = 5;
            let startPage = Math.max(1, currentPage - Math.floor(maxVisiblePages / 2));
            let endPage = Math.min(totalPages, startPage + maxVisiblePages - 1);

            if (endPage - startPage < maxVisiblePages - 1) {
                startPage = Math.max(1, endPage - maxVisiblePages + 1);
            }

            if (startPage > 1) {
                const firstBtn = createPaginationButton('1', true, () => goToPage(1));
                paginationContainer.appendChild(firstBtn);
                if (startPage > 2) {
                    const ellipsis = document.createElement('span');
                    ellipsis.textContent = '...';
                    ellipsis.style.padding = '4px 8px';
                    paginationContainer.appendChild(ellipsis);
                }
            }

            for (let i = startPage; i <= endPage; i++) {
                const pageBtn = createPaginationButton(String(i), true, () => goToPage(i), i === currentPage);
                paginationContainer.appendChild(pageBtn);
            }

            if (endPage < totalPages) {
                if (endPage < totalPages - 1) {
                    const ellipsis = document.createElement('span');
                    ellipsis.textContent = '...';
                    ellipsis.style.padding = '4px 8px';
                    paginationContainer.appendChild(ellipsis);
                }
                const lastBtn = createPaginationButton(String(totalPages), true, () => goToPage(totalPages));
                paginationContainer.appendChild(lastBtn);
            }

            // 下一页按钮
            const nextBtn = createPaginationButton('›', currentPage < totalPages, () => {
                if (currentPage < totalPages) {
                    currentPage++;
                    renderCurrentPage();
                    renderPagination();
                }
            });
            paginationContainer.appendChild(nextBtn);

            // 页面信息
            const info = document.createElement('span');
            info.style.cssText = 'padding: 4px 8px; color: #666; font-size: 12px;';
            info.textContent = `${currentPage} / ${totalPages}`;
            paginationContainer.appendChild(info);
        }

        /**
         * 创建分页按钮
         */
        function createPaginationButton(text, enabled, onClick, isActive = false) {
            const btn = document.createElement('button');
            btn.textContent = text;
            btn.style.cssText = `
                padding: 4px 12px;
                border: 1px solid #ddd;
                background: ${isActive ? '#3b82f6' : '#fff'};
                color: ${isActive ? '#fff' : '#333'};
                border-radius: 4px;
                cursor: ${enabled ? 'pointer' : 'not-allowed'};
                opacity: ${enabled ? '1' : '0.5'};
                transition: all 0.2s;
            `;

            if (enabled) {
                btn.addEventListener('click', onClick);
                btn.addEventListener('mouseenter', () => {
                    if (!isActive) {
                        btn.style.background = '#f3f4f6';
                    }
                });
                btn.addEventListener('mouseleave', () => {
                    btn.style.background = isActive ? '#3b82f6' : '#fff';
                });
            }

            return btn;
        }

        /**
         * 跳转到指定页
         */
        function goToPage(page) {
            if (page >= 1 && page <= totalPages) {
                currentPage = page;
                renderCurrentPage();
                renderPagination();
            }
        }

        /**
         * 设置数据
         */
        function setData(newData) {
            data = newData || [];
            totalPages = Math.ceil(data.length / pageSize) || 1;
            currentPage = 1;
            renderCurrentPage();
            renderPagination();
        }

        /**
         * 销毁实例
         */
        function destroy() {
            container.innerHTML = '';
        }

        return {
            setData,
            goToPage,
            destroy,
            getCurrentPage: () => currentPage,
            getTotalPages: () => totalPages
        };
    }

    return {
        create,
        createPaginated
    };
})();

// 导出模块
if (typeof module !== 'undefined' && module.exports) {
    module.exports = VirtualList;
}
